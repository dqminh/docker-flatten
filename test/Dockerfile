FROM ubuntu
MAINTAINER dqminh "dqminh89@gmail.com"
 
RUN echo Layer 0 >> layerfile.txt
RUN echo Layer 2 >> layerfile.txt
RUN apt-get update
RUN rm layerfile.txt
RUN echo Layer 1 >> layerfile1.txt
RUN echo Orig > layerfile1.txt
RUN echo "finished"

EXPOSE 123 234
CMD "/bin/echo" "Hello World"
